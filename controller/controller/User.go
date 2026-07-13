package controllers

import (
	"livesteam_app/controller/services"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterUser(c *gin.Context) {
	var input struct {
		Username string `json:"username" binding:"required"`
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err := services.RegisterUser(input.Username, input.Email, input.Password)
	if err != nil {
		if err == gorm.ErrRegistered {
			c.JSON(http.StatusConflict, gin.H{"error": "Email đã được sử dụng"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "User registered successfully. Please check your email for the verification code.",
	})
	return
}

func VerifyUser(c *gin.Context) {
	var input struct {
		Email string `json:"email" binding:"required,email"`
		Code  string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err := services.VerifyUser(input.Email, input.Code)
	if err != nil {
		if err.Error() == "user_not_found" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User not found"})
		} else if err.Error() == "user_already_verified" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User already verified"})
		} else if err.Error() == "otp_expired" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Verification code has expired"})
		} else if err.Error() == "invalid_verification_code" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid verification code"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "User verified successfully",
	})
	return

}
func Login(c *gin.Context) {
	var input struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	token, err := services.Login(input.Email, input.Password)
	if err != nil {
		if err.Error() == "user_not_found" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User not found"})
		} else if err.Error() == "invalid_password" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid password"})
		} else if err.Error() == "user_not_verified" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User not verified"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"token":  token,
	})
	return
}
func UpdateProfile(c *gin.Context) {
	var input struct {
		Fullname string `json:"fullname" binding:"required"`
		Avatar   string `json:"avatar" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err := services.SetupProfile(c.GetString("userID"), input.Fullname, input.Avatar)
	if err != nil {
		if err.Error() == "user_not_found" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User not found"})
		} else if err.Error() == "failed_to_update_profile" {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update profile"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Profile updated successfully",
	})
}
func ResetPasswordRequest(c *gin.Context) {
	var input struct {
		Email string `json:"email" binding:"required,email"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err := services.ResetPasswordRequest(input.Email)
	if err != nil {
		if err.Error() == "user_not_found" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User not found"})
		} else if err.Error() == "failed_to_generate_otp" {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate OTP"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Reset password request sent successfully",
	})
}
func ResetPassword(c *gin.Context) {
	var input struct{
		Email string `json:"email" binding:"required,email"`
		Code string `json:"code" binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err := services.ResetPassword(input.Email, input.Code, input.NewPassword)
	if err != nil {
		if err.Error() == "user_not_found" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User not found"})
		} else if err.Error() == "user_already_verified" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User already verified"})
		} else if err.Error() == "otp_expired" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Verification code has expired"})
		} else if err.Error() == "invalid_otp" {
			c.JSON(http.StatusBadRequest,gin.H{"error":"Invalid OTP"})
		} else if err.Error() == "failed_to_update_user" {
			c.JSON(http.StatusBadRequest,gin.H{"error":"Failed to update User"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Password reset successfully",
	})
}